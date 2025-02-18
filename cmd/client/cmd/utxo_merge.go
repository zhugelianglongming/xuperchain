// Copyright (c) 2021. Baidu Inc. All Rights Reserved.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/xuperchain/xupercore/bcs/ledger/xledger/state/utxo"
	"github.com/xuperchain/xupercore/lib/utils"

	"github.com/xuperchain/xuperchain/service/common"
	"github.com/xuperchain/xuperchain/service/pb"
	aclUtils "github.com/xuperchain/xupercore/kernel/permission/acl/utils"
)

// MergeUtxoCommand necessary parameter for merge utxo
type MergeUtxoCommand struct {
	cli *Cli
	cmd *cobra.Command
	// account will be merged
	account string
	// white merge a contract account, it can not be null
	accountPath string
}

// NewMergeUtxoCommand new an instance of merge utxo command
func NewMergeUtxoCommand(cli *Cli) *cobra.Command {
	c := new(MergeUtxoCommand)
	c.cli = cli
	c.cmd = &cobra.Command{
		Use:   "merge ",
		Short: "merge the utxo of an account or address.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.TODO()
			return c.mergeUtxo(ctx)
		},
	}
	c.addFlags()
	return c.cmd
}

func (c *MergeUtxoCommand) addFlags() {
	c.cmd.Flags().StringVarP(&c.account, "account", "A", "", "The account/address to be merged (default ./data/keys/address).")
	c.cmd.Flags().StringVarP(&c.accountPath, "accountPath", "P", "", "The account path, which is required for an account.")
}

func (c *MergeUtxoCommand) mergeUtxo(_ context.Context) error {
	if aclUtils.IsAccount(c.account) && c.accountPath == "" {
		return errors.New("accountPath can not be null because account is an Account name")
	}

	initAk, _ := readAddress(c.cli.RootOptions.Keys)
	if c.account == "" {
		c.account = initAk
	}

	tx := &pb.Transaction{
		Version:   utxo.TxVersion,
		Coinbase:  false,
		Nonce:     utils.GenNonce(),
		Timestamp: time.Now().UnixNano(),
		Initiator: initAk,
	}

	ct := &CommTrans{
		FrozenHeight: 0,
		Version:      utxo.TxVersion,
		From:         c.account,
		Args:         make(map[string][]byte),
		IsQuick:      false,
		ChainName:    c.cli.RootOptions.Name,
		Keys:         c.cli.RootOptions.Keys,
		XchainClient: c.cli.XchainClient(),
		CryptoType:   c.cli.RootOptions.Crypto,
	}

	txInputs, txOutput, err := ct.GenTxInputsWithMergeUTXO(context.Background())
	tx.TxInputs = txInputs
	// validation check
	if len(tx.TxInputs) == 0 {
		return errors.New("not enough available utxo to merge")
	}

	txOutputs := []*pb.TxOutput{}
	txOutputs = append(txOutputs, txOutput)
	tx.TxOutputs = txOutputs

	tx.AuthRequire, err = genAuthRequirement(c.account, c.accountPath)
	if err != nil {
		return fmt.Errorf("genAuthRequirement error: %s", err)
	}

	// preExe
	preExeRPCReq := &pb.InvokeRPCRequest{
		Bcname:   c.cli.RootOptions.Name,
		Requests: []*pb.InvokeRequest{},
		Header: &pb.Header{
			Logid: utils.GenLogId(),
		},
		Initiator:   initAk,
		AuthRequire: tx.AuthRequire,
	}
	preExeRes, err := ct.XchainClient.PreExec(context.Background(), preExeRPCReq)
	if err != nil {
		return err
	}
	tx.ContractRequests = preExeRes.GetResponse().GetRequests()
	tx.TxInputsExt = preExeRes.GetResponse().GetInputs()
	tx.TxOutputsExt = preExeRes.GetResponse().GetOutputs()

	tx.InitiatorSigns, err = ct.signTxForInitiator(tx)
	if err != nil {
		return err
	}
	tx.AuthRequireSigns, err = ct.signTx(tx, c.accountPath)
	if err != nil {
		return err
	}

	// calculate tx ID
	tx.Txid, err = common.MakeTxId(tx)
	if err != nil {
		return err
	}
	txID, err := ct.postTx(context.Background(), tx)
	if err != nil {
		return err
	}
	fmt.Println(txID)

	return nil
}
